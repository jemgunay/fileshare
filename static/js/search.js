var maxAutoCompleteSuggestions = 5; // Number of autocomplete results to show under tokenfield inputs.
var currentPage = 0; // Current pagination page (start at 0th page).
var tokenfieldTargetTrigger = true; // Prevents window resizing from triggering performSearch() by temporarily ignoring event listeners.

$(document).ready(function() {

    if (window.location.pathname === "/") {
        // init search/filter inputs
        $("#desc-search-input").val("").on("input", performSearch);

        performRequest(hostname + "/data?fetch=tags,people,file_types,dates", "GET", "", function (result) {
            var tokenfieldSets = [["tags", "#tags-search-input", false], ["people", "#people-search-input", false], ["file_types", "#type-search-input", true]];
            var parsedData = JSON.parse(result);

            initMetaDataFields(parsedData, tokenfieldSets, performSearch);

            // date pickers
            $("#min-date-picker, #max-date-picker").datetimepicker({
                format: "DD/MM/YYYY"
            });

            if (parsedData["dates"] == null) {
                var currentEpoch = Math.floor((new Date).getTime() / 1000);
                parsedData["dates"] = [currentEpoch, currentEpoch];
            }
            $("#min-date-picker").data("DateTimePicker").date(new Date(parseInt(parsedData["dates"][0]) * 1000));
            $("#max-date-picker").data("DateTimePicker").date(new Date(parseInt(parsedData["dates"][1]) * 1000));
            $("#min-date-picker, #max-date-picker").on("dp.change", performSearch);
        });

        // init pagination dropdown
        $("#count-search-input").change(performSearch);

        // init view toggle
        $("#view-search-input").bootstrapToggle({
            on: "Tiled View",
            off: "Detailed View",
            width: "100%",
            height: "21px"
        }).change(performSearch);

        initSearchTiles();
    }
});

// Pull required data from server & initialise search/filter inputs.
function initMetaDataFields(parsedData, tokenfieldSets, changeTarget) {
    // iterate over tokenfield types (tags, people & file_types) and initialise. If 3rd array value is true, tokenfield value will be populated pre-with all retrieved data.
    for (var i = 0; i < tokenfieldSets.length; i++) {
        var metaType = tokenfieldSets[i][0];
        var tagIDs = tokenfieldSets[i][1];
        var populateValue = tokenfieldSets[i][2];

        // check if data exists
        if (parsedData[metaType] != null) {
            var commaSeparatedData = parsedData[metaType].join();

            // set default input value
            if (populateValue) {
                $(tagIDs).val(commaSeparatedData);
            }
        }

        (function (tagIDs, parsedData, metaType) {
            $(tagIDs).tokenfield('destroy');
            $(tagIDs).tokenfield({
                autocomplete: {
                    source: function (request, response) {
                        if (parsedData[metaType] != null) {
                            var results = $.ui.autocomplete.filter(parsedData[metaType], request.term);

                            // remove already selected tokens from autocomplete results
                            var selectedTokens = $(tagIDs).tokenfield('getTokens', false);
                            var selectedTokenVals = [];
                            for (var i in selectedTokens) {
                                selectedTokenVals.push(selectedTokens[i]["label"]);
                            }
                            var unselectedResults = $(results).not(selectedTokenVals).get();

                            // limit autocomplete results
                            response(unselectedResults.slice(0, maxAutoCompleteSuggestions))
                        }
                    },
                    delay: 0
                },
                showAutocompleteOnFocus: true,
                createTokensOnBlur: true
            }).off("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken")
                .on("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken", function() {
                if (tokenfieldTargetTrigger && changeTarget != null) {
                    changeTarget();
                }
            });

            // fix tokenfield newline bug on resize
            $(window).off("resize").on("resize", function() {
                tokenfieldTargetTrigger = false;
                var tokens = $(tagIDs).tokenfield('getTokens');
                $(tagIDs).tokenfield('setTokens', []);
                $(tagIDs).tokenfield('setTokens', tokens);
                tokenfieldTargetTrigger = true;
            });
        }(tagIDs, parsedData, metaType));
    }
}

// Perform search/filter request.
function performSearch() {
    var request = constructSearchURL();

    // perform search request
    performRequest(hostname + request, "GET", "", function(html) {
        $(".results-window").fadeOut(100, function () {
            $(this).empty().append(html).fadeIn(100);
            initSearchTiles();
        });
    });
}

// Collect & format parameters from inputs, then construct URL for search request.
function constructSearchURL() {
    var dates = [$("#min-date-picker").data("DateTimePicker").date(), $("#max-date-picker").data("DateTimePicker").date()];
    if (dates[0]) { dates[0] = dates[0].unix() }
    if (dates[1]) { dates[1] = dates[1].unix() }
    var tokenfieldTags = [$("#tags-search-input").tokenfield('getTokensList', ",", false), $("#people-search-input").tokenfield('getTokensList', ",", false), $("#type-search-input").tokenfield('getTokensList', ",", false)];
    var format = "html_detailed";
    if ($("#view-search-input").is(":checked")) {
        format = "html_tiled";
    }
    var resultsPerPage = $("#count-search-input :selected").val();

    var request = "/search?desc=" + $("#desc-search-input").val() + "&min_date=" + dates[0] + "&max_date=" + dates[1] + "&tags=" + tokenfieldTags[0] + "&people=" + tokenfieldTags[1];
    request += "&file_types=" + tokenfieldTags[2] + "&format=" + format + "&results_per_page=" + resultsPerPage + "&page=" + currentPage;
    return request;
}

// Init search result tiles.
function initSearchTiles() {
    // freewall tiled images
    var wall = new Freewall("#freewall");
    wall.reset({
        selector: '.free-wall-tile',
        animate: true,
        cellW: 200,
        cellH: 'auto',
        onResize: function() {
            wall.fitWidth();
        }
    });
    wall.container.find('.free-wall-tile img').load(function() {
        wall.fitWidth();
    });

    // view overlay window on click
    $("#search-results-panel a").on("click", function(e) {
        e.preventDefault();
        $("#overlay-window").attr("opacity", 0).attr("display", "initial").fadeIn(100);
        $('body').css('overflow', 'hidden'); // prevent background scroll

        setOverlayMemory($(this).attr("data-UUID"));

        // key presses
        $(document).keyup(function(e) {
            // escape key
            if (e.which === 27) {
                if ($("#overlay-window").attr("display") !== "none") {
                    $(document).unbind("keyup");
                    $('body').css('overflow','auto');

                    $("#overlay-window").fadeOut(100, function() {
                        $("#overlay-window").attr("display", "none");
                    });
                }
            }
            // left key (previous memory)
            else if (e.which === 37) {

            }
            // right key (next memory)
            else if (e.which === 39) {

            }
        });

        // close button
        $("#overlay-close-btn").on("click", function() {
            $(document).unbind("keyup");
            $('body').css('overflow','auto');

            $("#overlay-window").fadeOut(100, function() {
                $("#overlay-window").attr("display", "none");
            });
        });
    });
}

// Set overlay window current memory.
function setOverlayMemory(memoryUUID) {
    $("#overlay-content").empty();

    performRequest(hostname + "/data", "POST", {type: "file", UUID: memoryUUID, format: "html"}, function(response) {
        $("#overlay-content").empty().append(response);
    });
}