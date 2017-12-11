var hostname = location.protocol + '//' + location.host;

$(document).ready(function() {
    // send message on button click
    $("form#upload-form").submit(function(e) {
        e.preventDefault();
        var formData = new FormData(this);
        performRequest(hostname + "/upload/", "POST", formData, function(html) {
            window.location = "/"
        });
    });
    
    // description
    $("#desc-search-input").on("input", function() {
        performRequest(hostname + "/search?desc=" + $(this).val() + "&format=true", "GET", "", function(html) {
            $("#results-window").empty().append(html)
        });
    });
    
    // tags
    performRequest(hostname + "/data?tags=true", "GET", "", function(data) {
        var data = ["Amsterdam",
            "London",
            "Paris",
            "Washington",
            "New York",
            "New Jersey",
            "New Orleans",
            "Los Angeles",
            "Sydney",
            "Melbourne",
            "Canberra",
            "Beijing",
            "New Delhi",
            "Kathmandu",
            "Cairo",
            "Cape Town",
            "Kinshasa"];
        var citynames = new Bloodhound({
            datumTokenizer: Bloodhound.tokenizers.obj.whitespace('name'),
            queryTokenizer: Bloodhound.tokenizers.whitespace,
            local: $.map(data, function (city) {
                return {
                    name: city
                };
            })
        });
        citynames.initialize();

        $('.tags-container input').tagsinput({
            typeaheadjs: [{
                minLength: 1,
                highlight: true,
            },{
                minlength: 1,
                name: 'citynames',
                displayKey: 'name',
                valueKey: 'name',
                source: citynames.ttAdapter()
            }],
            freeInput: true
        });
    });
});

// Perform AJAX request.
function performRequest(URL, httpMethod, data, resultMethod) {
    $.ajax({
        url: URL,
        type: httpMethod,
        dataType: 'text',
        data: data,
        error: function(e) {
            console.log(e);
        },
        success: function(e) {
            resultMethod(e);
        },
        cache: false,
        contentType: false,
        processData: false
    });
}