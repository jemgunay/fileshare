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
    
    // search
    $("#search-input").on("input", function() {
        performRequest(hostname + "/search?desc=" + $(this).val() + "&format=true", "GET", "", function(html) {
            $("#results-window").empty().append(html)
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